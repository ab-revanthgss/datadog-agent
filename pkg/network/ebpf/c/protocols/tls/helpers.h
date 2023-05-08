#ifndef __TLS_HELPERS_H
#define __TLS_HELPERS_H

#include "protocols/classification/shared-tracer-maps.h"

static __always_inline bool is_tls_connection_cached(conn_tuple_t *t) {
    if (bpf_map_lookup_elem(&tls_connection, t) != NULL) {
        return true;
    }
    return false;
}

#endif
