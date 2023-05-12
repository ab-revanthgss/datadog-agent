#include "kconfig.h"
#include <linux/version.h>

#include "bpf_helpers.h"
#include "bpf_endian.h"
#include "bpf_telemetry.h"

#include "offsets.h"
#include "conntrack.h"
#include "conntrack/maps.h"
#include "ip.h"
#include "ipv6.h"

SEC("kprobe/__nf_conntrack_hash_insert")
int kprobe___nf_conntrack_hash_insert(struct pt_regs* ctx) {
    struct nf_conn *ct = (struct nf_conn*)PT_REGS_PARM1(ctx);

    u32 status = ct_status(ct);
    if (!(status&IPS_CONFIRMED) || !(status&IPS_NAT_MASK)) {
        return 0;
    }

    log_debug("kprobe/__nf_conntrack_hash_insert: netns: %u, status: %x", get_netns(ct), status);

    conntrack_tuple_t orig = {}, reply = {};
    if (nf_conn_to_conntrack_tuples(ct, &orig, &reply) != 0) {
        return 0;
    }

    bpf_map_update_with_telemetry(conntrack, &orig, &reply, BPF_ANY);
    bpf_map_update_with_telemetry(conntrack, &reply, &orig, BPF_ANY);
    increment_telemetry_registers_count();

    return 0;
}

SEC("kprobe/ctnetlink_fill_info")
int kprobe_ctnetlink_fill_info(struct pt_regs* ctx) {
    u32 pid = bpf_get_current_pid_tgid() >> 32;
    if (pid != systemprobe_pid()) {
        log_debug("skipping kprobe/ctnetlink_fill_info invocation from non-system-probe process");
        return 0;
    }

    struct nf_conn *ct = (struct nf_conn*)PT_REGS_PARM5(ctx);

    u32 status = ct_status(ct);
    if (!(status&IPS_CONFIRMED) || !(status&IPS_NAT_MASK)) {
        return 0;
    }

    log_debug("kprobe/ctnetlink_fill_info: netns: %u, status: %x", get_netns(ct), status);

    conntrack_tuple_t orig = {}, reply = {};
    if (nf_conn_to_conntrack_tuples(ct, &orig, &reply) != 0) {
        return 0;
    }

    bpf_map_update_with_telemetry(conntrack, &orig, &reply, BPF_ANY);
    bpf_map_update_with_telemetry(conntrack, &reply, &orig, BPF_ANY);
    increment_telemetry_registers_count();

    return 0;
}

// This number will be interpreted by elf-loader to set the current running kernel version
__u32 _version SEC("version") = 0xFFFFFFFE; // NOLINT(bugprone-reserved-identifier)

char _license[] SEC("license") = "GPL"; // NOLINT(bugprone-reserved-identifier)
