// Code generated by cmd/cgo -godefs; DO NOT EDIT.
// cgo -godefs -- -I c -I ../../ebpf/c -fsigned-char kprobe_types.go

package ebpf

type ConnTuple struct {
	Saddr_h  uint64
	Saddr_l  uint64
	Daddr_h  uint64
	Daddr_l  uint64
	Sport    uint16
	Dport    uint16
	Netns    uint32
	Pid      uint32
	Metadata uint32
}
type TCPStats struct {
	Retransmits       uint32
	Rtt               uint32
	Rtt_var           uint32
	State_transitions uint16
	Pad_cgo_0         [2]byte
}
type ConnStats struct {
	Sent_bytes   uint64
	Recv_bytes   uint64
	Timestamp    uint64
	Flags        uint32
	Cookie       uint32
	Sent_packets uint64
	Recv_packets uint64
	Direction    uint8
	Protocol     uint8
	Pad_cgo_0    [6]byte
}
type Conn struct {
	Tup        ConnTuple
	Conn_stats ConnStats
	Tcp_stats  TCPStats
}
type Batch struct {
	C0  Conn
	C1  Conn
	C2  Conn
	C3  Conn
	Len uint16
	Id  uint64
}
type Telemetry struct {
	Tcp_failed_connect  uint64
	Tcp_sent_miscounts  uint64
	Missed_tcp_close    uint64
	Missed_udp_close    uint64
	Udp_sends_processed uint64
	Udp_sends_missed    uint64
	Udp_dropped_conns   uint64
	Invalid_tcp_retrans uint64
}
type PortBinding struct {
	Netns     uint32
	Port      uint16
	Pad_cgo_0 [2]byte
}
type PIDFD struct {
	Pid uint32
	Fd  uint32
}
type UDPRecvSock struct {
	Sk  *_Ctype_struct_sock
	Msg *_Ctype_struct_msghdr
}
type BindSyscallArgs struct {
	Addr *_Ctype_struct_sockaddr
	Sk   *_Ctype_struct_sock
}

type _Ctype_struct_sock uint64
type _Ctype_struct_msghdr uint64
type _Ctype_struct_sockaddr uint64

type TCPState uint8

const (
	Established TCPState = 0x1
	Close       TCPState = 0x7
)

type ConnFlags uint32

const (
	LInit   ConnFlags = 0x1
	RInit   ConnFlags = 0x2
	Assured ConnFlags = 0x4
)

const BatchSize = 0x4
const SizeofBatch = 0x1f0

type ClassificationProgram = uint32

const (
	ClassificationQueues ClassificationProgram = 0x0
	ClassificationDBs    ClassificationProgram = 0x1
)
