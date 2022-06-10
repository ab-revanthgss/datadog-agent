// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package invocationlifecycle

import (
	"os"
	"testing"
	"time"

	"github.com/DataDog/datadog-agent/pkg/aggregator"
	"github.com/DataDog/datadog-agent/pkg/metrics"
	"github.com/DataDog/datadog-agent/pkg/serverless/logs"
	"github.com/DataDog/datadog-agent/pkg/serverless/trace/inferredspan"
	"github.com/DataDog/datadog-agent/pkg/trace/api"
	"github.com/DataDog/datadog-agent/pkg/trace/pb"
	"github.com/DataDog/datadog-agent/pkg/trace/sampler"

	"github.com/stretchr/testify/assert"
)

func TestGenerateEnhancedErrorMetricOnInvocationEnd(t *testing.T) {
	extraTags := &logs.Tags{
		Tags: []string{"functionname:test-function"},
	}
	mockProcessTrace := func(*api.Payload) {}
	mockDetectLambdaLibrary := func() bool { return true }
	demux := aggregator.InitTestAgentDemultiplexerWithFlushInterval(time.Hour)

	endInvocationTime := time.Now()
	endDetails := InvocationEndDetails{EndTime: endInvocationTime, IsError: true}

	testProcessor := LifecycleProcessor{
		ExtraTags:           extraTags,
		ProcessTrace:        mockProcessTrace,
		DetectLambdaLibrary: mockDetectLambdaLibrary,
		Demux:               demux,
	}
	go testProcessor.OnInvokeEnd(&endDetails)

	generatedMetrics := demux.WaitForSamples(time.Millisecond * 250)

	assert.Equal(t, generatedMetrics, []metrics.MetricSample{{
		Name:       "aws.lambda.enhanced.errors",
		Value:      1.0,
		Mtype:      metrics.DistributionType,
		Tags:       extraTags.Tags,
		SampleRate: 1,
		Timestamp:  float64(endInvocationTime.UnixNano()) / float64(time.Second),
	}})
}

func TestStartExecutionSpanNoLambdaLibrary(t *testing.T) {
	extraTags := &logs.Tags{
		Tags: []string{"functionname:test-function"},
	}
	demux := aggregator.InitTestAgentDemultiplexerWithFlushInterval(time.Hour)
	mockProcessTrace := func(*api.Payload) {}
	mockDetectLambdaLibrary := func() bool { return false }

	eventPayload := `a5a{"resource":"/users/create","path":"/users/create","httpMethod":"GET","headers":{"Accept":"*/*","Accept-Encoding":"gzip","x-datadog-parent-id":"1480558859903409531","x-datadog-sampling-priority":"1","x-datadog-trace-id":"5736943178450432258"}}0`
	startInvocationTime := time.Now()
	startDetails := InvocationStartDetails{StartTime: startInvocationTime, InvokeEventRawPayload: eventPayload}

	testProcessor := LifecycleProcessor{
		ExtraTags:           extraTags,
		ProcessTrace:        mockProcessTrace,
		DetectLambdaLibrary: mockDetectLambdaLibrary,
		Demux:               demux,
	}

	testProcessor.OnInvokeStart(&startDetails)

	assert.NotNil(t, testProcessor.GetExecutionInfo())

	assert.Equal(t, uint64(0), testProcessor.GetExecutionInfo().SpanID)
	assert.Equal(t, uint64(5736943178450432258), testProcessor.GetExecutionInfo().TraceID)
	assert.Equal(t, uint64(1480558859903409531), testProcessor.GetExecutionInfo().parentID)
	assert.Equal(t, sampler.SamplingPriority(1), testProcessor.GetExecutionInfo().SamplingPriority)
	assert.Equal(t, startInvocationTime, testProcessor.GetExecutionInfo().startTime)
}

func TestStartExecutionSpanWithLambdaLibrary(t *testing.T) {
	extraTags := &logs.Tags{
		Tags: []string{"functionname:test-function"},
	}
	demux := aggregator.InitTestAgentDemultiplexerWithFlushInterval(time.Hour)
	mockProcessTrace := func(*api.Payload) {}
	mockDetectLambdaLibrary := func() bool { return true }

	startInvocationTime := time.Now()
	startDetails := InvocationStartDetails{StartTime: startInvocationTime}

	testProcessor := LifecycleProcessor{
		ExtraTags:           extraTags,
		ProcessTrace:        mockProcessTrace,
		DetectLambdaLibrary: mockDetectLambdaLibrary,
		Demux:               demux,
	}
	testProcessor.OnInvokeStart(&startDetails)

	assert.NotEqual(t, 0, testProcessor.GetExecutionInfo().SpanID)
	assert.NotEqual(t, 0, testProcessor.GetExecutionInfo().TraceID)
	assert.Equal(t, startInvocationTime, testProcessor.GetExecutionInfo().startTime)
}

func TestEndExecutionSpanNoLambdaLibrary(t *testing.T) {
	defer os.Unsetenv(functionNameEnvVar)
	os.Setenv(functionNameEnvVar, "TestFunction")

	extraTags := &logs.Tags{
		Tags: []string{"functionname:test-function"},
	}
	demux := aggregator.InitTestAgentDemultiplexerWithFlushInterval(time.Hour)
	mockDetectLambdaLibrary := func() bool { return false }

	var tracePayload *api.Payload
	mockProcessTrace := func(payload *api.Payload) {
		tracePayload = payload
	}
	startInvocationTime := time.Now()
	duration := 1 * time.Second
	endInvocationTime := startInvocationTime.Add(duration)
	endDetails := InvocationEndDetails{EndTime: endInvocationTime, IsError: false}
	samplingPriority := sampler.SamplingPriority(1)

	testProcessor := LifecycleProcessor{
		ExtraTags:           extraTags,
		ProcessTrace:        mockProcessTrace,
		DetectLambdaLibrary: mockDetectLambdaLibrary,
		Demux:               demux,
		requestHandler: &RequestHandler{
			executionInfo: &ExecutionStartInfo{
				startTime:        startInvocationTime,
				TraceID:          123,
				SpanID:           1,
				parentID:         3,
				SamplingPriority: samplingPriority,
			},
			triggerTags: make(map[string]string),
		},
	}
	testProcessor.OnInvokeEnd(&endDetails)
	executionChunkPriority := tracePayload.TracerPayload.Chunks[0].Priority
	executionSpan := tracePayload.TracerPayload.Chunks[0].Spans[0]
	assert.Equal(t, "aws.lambda", executionSpan.Name)
	assert.Equal(t, "aws.lambda", executionSpan.Service)
	assert.Equal(t, "TestFunction", executionSpan.Resource)
	assert.Equal(t, "serverless", executionSpan.Type)
	assert.Equal(t, testProcessor.GetExecutionInfo().TraceID, executionSpan.TraceID)
	assert.Equal(t, testProcessor.GetExecutionInfo().SpanID, executionSpan.SpanID)
	assert.Equal(t, testProcessor.GetExecutionInfo().parentID, executionSpan.ParentID)
	assert.Equal(t, int32(testProcessor.GetExecutionInfo().SamplingPriority), executionChunkPriority)
	assert.Equal(t, startInvocationTime.UnixNano(), executionSpan.Start)
	assert.Equal(t, duration.Nanoseconds(), executionSpan.Duration)
}

func TestEndExecutionSpanWithLambdaLibrary(t *testing.T) {
	extraTags := &logs.Tags{
		Tags: []string{"functionname:test-function"},
	}
	demux := aggregator.InitTestAgentDemultiplexerWithFlushInterval(time.Hour)
	mockDetectLambdaLibrary := func() bool { return true }

	var tracePayload *api.Payload
	mockProcessTrace := func(payload *api.Payload) {
		tracePayload = payload
	}
	startInvocationTime := time.Now()
	duration := 1 * time.Second
	endInvocationTime := startInvocationTime.Add(duration)
	endDetails := InvocationEndDetails{EndTime: endInvocationTime, IsError: false}

	testProcessor := LifecycleProcessor{
		ExtraTags:           extraTags,
		ProcessTrace:        mockProcessTrace,
		DetectLambdaLibrary: mockDetectLambdaLibrary,
		Demux:               demux,
		requestHandler: &RequestHandler{
			executionInfo: &ExecutionStartInfo{
				startTime: startInvocationTime,
				TraceID:   123,
				SpanID:    1,
			},
			triggerTags: make(map[string]string),
		},
	}

	testProcessor.OnInvokeEnd(&endDetails)

	assert.Equal(t, (*api.Payload)(nil), tracePayload)
}

func TestCompleteInferredSpanWithStartTime(t *testing.T) {
	defer os.Unsetenv(functionNameEnvVar)
	os.Setenv(functionNameEnvVar, "TestFunction")

	extraTags := &logs.Tags{
		Tags: []string{"functionname:test-function"},
	}
	demux := aggregator.InitTestAgentDemultiplexerWithFlushInterval(time.Hour)
	mockDetectLambdaLibrary := func() bool { return false }

	var tracePayload *api.Payload
	mockProcessTrace := func(payload *api.Payload) {
		tracePayload = payload
	}
	startInferredSpan := time.Now()
	startInvocationTime := startInferredSpan.Add(250 * time.Millisecond)
	duration := 1 * time.Second
	endInvocationTime := startInvocationTime.Add(duration)
	endDetails := InvocationEndDetails{EndTime: endInvocationTime, IsError: false}
	samplingPriority := sampler.SamplingPriority(1)

	testProcessor := LifecycleProcessor{
		ExtraTags:            extraTags,
		ProcessTrace:         mockProcessTrace,
		DetectLambdaLibrary:  mockDetectLambdaLibrary,
		Demux:                demux,
		InferredSpansEnabled: true,
		requestHandler: &RequestHandler{
			executionInfo: &ExecutionStartInfo{
				startTime:        startInvocationTime,
				TraceID:          123,
				SpanID:           1,
				parentID:         3,
				SamplingPriority: samplingPriority,
			},
			triggerTags: make(map[string]string),
			inferredSpan: &inferredspan.InferredSpan{
				CurrentInvocationStartTime: startInferredSpan,
				Span: &pb.Span{
					TraceID: 123,
					SpanID:  3,
					Start:   startInferredSpan.UnixNano(),
				},
			},
		},
	}

	testProcessor.OnInvokeEnd(&endDetails)

	completedInferredSpan := tracePayload.TracerPayload.Chunks[0].Spans[0]
	assert.Equal(t, testProcessor.GetInferredSpan().Span.Start, completedInferredSpan.Start)
}

func TestCompleteInferredSpanWithOutStartTime(t *testing.T) {
	defer os.Unsetenv(functionNameEnvVar)
	os.Setenv(functionNameEnvVar, "TestFunction")

	extraTags := &logs.Tags{
		Tags: []string{"functionname:test-function"},
	}
	demux := aggregator.InitTestAgentDemultiplexerWithFlushInterval(time.Hour)
	mockDetectLambdaLibrary := func() bool { return false }

	var tracePayload *api.Payload
	mockProcessTrace := func(payload *api.Payload) {
		tracePayload = payload
	}
	startInvocationTime := time.Now()
	duration := 1 * time.Second
	endInvocationTime := startInvocationTime.Add(duration)
	endDetails := InvocationEndDetails{EndTime: endInvocationTime, IsError: false}
	samplingPriority := sampler.SamplingPriority(1)

	testProcessor := LifecycleProcessor{
		ExtraTags:            extraTags,
		ProcessTrace:         mockProcessTrace,
		DetectLambdaLibrary:  mockDetectLambdaLibrary,
		Demux:                demux,
		InferredSpansEnabled: true,
		requestHandler: &RequestHandler{
			executionInfo: &ExecutionStartInfo{
				startTime:        startInvocationTime,
				TraceID:          123,
				SpanID:           1,
				parentID:         3,
				SamplingPriority: samplingPriority,
			},
			triggerTags: make(map[string]string),
			inferredSpan: &inferredspan.InferredSpan{
				CurrentInvocationStartTime: time.Time{},
				Span: &pb.Span{
					TraceID: 123,
					SpanID:  3,
					Start:   0,
				},
			},
		},
	}

	testProcessor.OnInvokeEnd(&endDetails)

	// If our logic is correct this will actually be the execution span
	// and the start time is expected to be the invocation start time,
	// not the inferred span start time.
	completedInferredSpan := tracePayload.TracerPayload.Chunks[0].Spans[0]
	assert.Equal(t, startInvocationTime.UnixNano(), completedInferredSpan.Start)
}
func TestTriggerTypesLifecycleEventForAPIGatewayRest(t *testing.T) {
	os.Setenv("AWS_REGION", "us-east-1")
	startDetails := &InvocationStartDetails{
		InvokeEventRawPayload: string(getEventFromFile("api-gateway.json")),
	}

	testProcessor := &LifecycleProcessor{
		DetectLambdaLibrary: func() bool { return false },
	}

	testProcessor.OnInvokeStart(startDetails)
	assert.Equal(t, map[string]string{
		"function_trigger.event_source_arn": "arn:aws:apigateway:us-east-1::/restapis/1234567890/stages/prod",
		"http.method":                       "POST",
		"http.url":                          "70ixmpl4fl.execute-api.us-east-2.amazonaws.com",
		"http.url_details.path":             "/prod/path/to/resource",
		"function_trigger.event_source":     "api-gateway",
	}, testProcessor.GetTags())
}

func TestTriggerTypesLifecycleEventForAPIGatewayNonProxy(t *testing.T) {
	os.Setenv("AWS_REGION", "us-east-1")
	startDetails := &InvocationStartDetails{
		InvokeEventRawPayload: string(getEventFromFile("api-gateway-non-proxy.json")),
	}

	testProcessor := &LifecycleProcessor{
		DetectLambdaLibrary: func() bool { return false },
		ProcessTrace:        func(*api.Payload) {},
	}

	testProcessor.OnInvokeStart(startDetails)
	testProcessor.OnInvokeEnd(&InvocationEndDetails{
		RequestID:          "test-request-id",
		ResponseRawPayload: []byte(`{"statusCode": 200}`),
	})
	assert.Equal(t, map[string]string{
		"function_trigger.event_source_arn": "arn:aws:apigateway:us-east-1::/restapis/lgxbo6a518/stages/dev",
		"http.method":                       "GET",
		"http.url":                          "lgxbo6a518.execute-api.sa-east-1.amazonaws.com",
		"http.url_details.path":             "/dev/http/get",
		"request_id":                        "test-request-id",
		"http.status_code":                  "200",
		"function_trigger.event_source":     "api-gateway",
	}, testProcessor.GetTags())
}

func TestTriggerTypesLifecycleEventForAPIGatewayWebsocket(t *testing.T) {
	os.Setenv("AWS_REGION", "us-east-1")
	startDetails := &InvocationStartDetails{
		InvokeEventRawPayload: string(getEventFromFile("api-gateway-websocket-default.json")),
	}

	testProcessor := &LifecycleProcessor{
		DetectLambdaLibrary: func() bool { return false },
		ProcessTrace:        func(*api.Payload) {},
	}

	testProcessor.OnInvokeStart(startDetails)
	testProcessor.OnInvokeEnd(&InvocationEndDetails{
		RequestID:          "test-request-id",
		ResponseRawPayload: []byte(`{"statusCode": 200}`),
	})
	assert.Equal(t, map[string]string{
		"function_trigger.event_source_arn": "arn:aws:apigateway:us-east-1::/restapis/p62c47itsb/stages/dev",
		"request_id":                        "test-request-id",
		"http.status_code":                  "200",
		"function_trigger.event_source":     "api-gateway",
	}, testProcessor.GetTags())
}

func TestTriggerTypesLifecycleEventForALB(t *testing.T) {
	os.Setenv("AWS_REGION", "us-east-1")
	startDetails := &InvocationStartDetails{
		InvokeEventRawPayload: string(getEventFromFile("application-load-balancer.json")),
	}

	testProcessor := &LifecycleProcessor{
		DetectLambdaLibrary: func() bool { return false },
		ProcessTrace:        func(*api.Payload) {},
	}

	testProcessor.OnInvokeStart(startDetails)
	testProcessor.OnInvokeEnd(&InvocationEndDetails{
		RequestID:          "test-request-id",
		ResponseRawPayload: []byte(`{"statusCode": 200}`),
	})
	assert.Equal(t, map[string]string{
		"function_trigger.event_source_arn": "arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/lambda-xyz/123abc",
		"request_id":                        "test-request-id",
		"http.status_code":                  "200",
		"http.method":                       "GET",
		"http.url_details.path":             "/lambda",
		"function_trigger.event_source":     "application-load-balancer",
	}, testProcessor.GetTags())
}

func TestTriggerTypesLifecycleEventForCloudwatch(t *testing.T) {
	os.Setenv("AWS_REGION", "us-east-1")
	startDetails := &InvocationStartDetails{
		InvokeEventRawPayload: string(getEventFromFile("cloudwatch-events.json")),
	}

	testProcessor := &LifecycleProcessor{
		DetectLambdaLibrary: func() bool { return false },
		ProcessTrace:        func(*api.Payload) {},
	}

	testProcessor.OnInvokeStart(startDetails)
	testProcessor.OnInvokeEnd(&InvocationEndDetails{
		RequestID: "test-request-id",
	})
	assert.Equal(t, map[string]string{
		"function_trigger.event_source_arn": "arn:aws:events:us-east-1:123456789012:rule/ExampleRule",
		"request_id":                        "test-request-id",
		"function_trigger.event_source":     "cloudwatch-events",
	}, testProcessor.GetTags())
}

func TestTriggerTypesLifecycleEventForDynamoDB(t *testing.T) {
	os.Setenv("AWS_REGION", "us-east-1")
	startDetails := &InvocationStartDetails{
		InvokeEventRawPayload: string(getEventFromFile("dynamodb.json")),
	}

	testProcessor := &LifecycleProcessor{
		DetectLambdaLibrary: func() bool { return false },
		ProcessTrace:        func(*api.Payload) {},
	}

	testProcessor.OnInvokeStart(startDetails)
	testProcessor.OnInvokeEnd(&InvocationEndDetails{
		RequestID: "test-request-id",
	})
	assert.Equal(t, map[string]string{
		"function_trigger.event_source_arn": "arn:aws:dynamodb:us-east-1:123456789012:table/ExampleTableWithStream/stream/2015-06-27T00:48:05.899",
		"request_id":                        "test-request-id",
		"function_trigger.event_source":     "dynamodb",
	}, testProcessor.GetTags())
}

func TestTriggerTypesLifecycleEventForKinesis(t *testing.T) {
	os.Setenv("AWS_REGION", "us-east-1")
	startDetails := &InvocationStartDetails{
		InvokeEventRawPayload: string(getEventFromFile("kinesis-batch.json")),
	}

	testProcessor := &LifecycleProcessor{
		DetectLambdaLibrary: func() bool { return false },
		ProcessTrace:        func(*api.Payload) {},
	}

	testProcessor.OnInvokeStart(startDetails)
	testProcessor.OnInvokeEnd(&InvocationEndDetails{
		RequestID: "test-request-id",
	})
	assert.Equal(t, map[string]string{
		"function_trigger.event_source_arn": "arn:aws:kinesis:sa-east-1:601427279990:stream/kinesisStream",
		"request_id":                        "test-request-id",
		"function_trigger.event_source":     "kinesis",
	}, testProcessor.GetTags())
}

func TestTriggerTypesLifecycleEventForS3(t *testing.T) {
	os.Setenv("AWS_REGION", "us-east-1")
	startDetails := &InvocationStartDetails{
		InvokeEventRawPayload: string(getEventFromFile("s3.json")),
	}

	testProcessor := &LifecycleProcessor{
		DetectLambdaLibrary: func() bool { return false },
		ProcessTrace:        func(*api.Payload) {},
	}

	testProcessor.OnInvokeStart(startDetails)
	testProcessor.OnInvokeEnd(&InvocationEndDetails{
		RequestID: "test-request-id",
	})
	assert.Equal(t, map[string]string{
		"function_trigger.event_source_arn": "aws:s3:sample:event:source",
		"request_id":                        "test-request-id",
		"function_trigger.event_source":     "s3",
	}, testProcessor.GetTags())
}

func TestTriggerTypesLifecycleEventForSNS(t *testing.T) {
	os.Setenv("AWS_REGION", "us-east-1")
	startDetails := &InvocationStartDetails{
		InvokeEventRawPayload: string(getEventFromFile("sns-batch.json")),
	}

	testProcessor := &LifecycleProcessor{
		DetectLambdaLibrary: func() bool { return false },
		ProcessTrace:        func(*api.Payload) {},
	}

	testProcessor.OnInvokeStart(startDetails)
	testProcessor.OnInvokeEnd(&InvocationEndDetails{
		RequestID: "test-request-id",
	})
	assert.Equal(t, map[string]string{
		"function_trigger.event_source_arn": "arn:aws:sns:sa-east-1:601427279990:serverlessTracingTopicPy",
		"request_id":                        "test-request-id",
		"function_trigger.event_source":     "sns",
	}, testProcessor.GetTags())
}

func TestTriggerTypesLifecycleEventForSQS(t *testing.T) {
	os.Setenv("AWS_REGION", "us-east-1")
	startDetails := &InvocationStartDetails{
		InvokeEventRawPayload: string(getEventFromFile("sqs-batch.json")),
	}

	testProcessor := &LifecycleProcessor{
		DetectLambdaLibrary: func() bool { return false },
		ProcessTrace:        func(*api.Payload) {},
	}

	testProcessor.OnInvokeStart(startDetails)
	testProcessor.OnInvokeEnd(&InvocationEndDetails{
		RequestID: "test-request-id",
	})
	assert.Equal(t, map[string]string{
		"function_trigger.event_source_arn": "arn:aws:sqs:sa-east-1:601427279990:InferredSpansQueueNode",
		"request_id":                        "test-request-id",
		"function_trigger.event_source":     "sqs",
	}, testProcessor.GetTags())
}

// Helper function for reading test file
func getEventFromFile(filename string) []byte {
	event, err := os.ReadFile("../trace/testdata/event_samples/" + filename)
	if err != nil {
		panic(err)
	}
	return event
}
