package cloudservice

import (
	"os"
	"strings"
)

// ContainerApp has helper functions for getting specific Azure Container App data
type ContainerApp struct{}

const (
	// ContainerAppNameEnvVar is the environment variable that is present when we're
	// running in Azure App Container.
	ContainerAppNameEnvVar = "CONTAINER_APP_NAME"

	ContainerAppDNSSuffix = "CONTAINER_APP_ENV_DNS_SUFFIX"

	ContainerAppRevision = "CONTAINER_APP_REVISION"
)

// GetTags returns a map of Azure-related tags
func (c *ContainerApp) GetTags() map[string]string {
	appName := os.Getenv(ContainerAppNameEnvVar)
	appDNSSuffix := os.Getenv(ContainerAppDNSSuffix)

	appDNSSuffixTokens := strings.Split(appDNSSuffix, ".")
	region := appDNSSuffixTokens[len(appDNSSuffixTokens)-3]

	revision := os.Getenv(ContainerAppRevision)

	return map[string]string{
		"app_name": appName,
		"region":   region,
		"revision": revision,
	}
}

// GetOrigin returns the `origin` attribute type for the given
// cloud service.
func (c *ContainerApp) GetOrigin() string {
	return "containerapp"
}

func isContainerAppService() bool {
	_, exists := os.LookupEnv(ContainerAppNameEnvVar)
	return exists
}
