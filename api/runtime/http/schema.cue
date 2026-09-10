package bees

#Duration = =~"^(?:[0-9]+(?:ns|us|ms|s|m|h)|[0-9]+)$"

// http runtime configuration schema
http: {
    // Bind address. Common forms:
    //  - ":http" (named port)
    //  - ":8443"
    //  - "0.0.0.0:8080"
    //  - "localhost:8443"
    addr: *":http" | string & =~"^(:[A-Za-z0-9_-]+|[^:]+:\\d+)$"

    // Durations (parseable by time.ParseDuration). Default values reflect the runtime defaults.
    read_timeout:  *"10s" | string & #Duration
    write_timeout: *"30s" | string & #Duration
    idle_timeout:  *"120s" | string & #Duration

    // TLS certificate and key files. Either both must be provided or neither.
    tls_cert_file: *"" | string
    tls_key_file:  *"" | string

    // Validation: require both TLS files if one is set.
    ((tls_cert_file == "") && (tls_key_file == "")) || ((tls_cert_file != "") && (tls_key_file != ""))
}
