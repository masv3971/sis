#!/bin/sh
# Development PKI: a root CA and one TLS server certificate per service in
# docker-compose.yaml. Modelled on SUNET/vc developer_tools/scripts/create_pki.sh.
#
# Output (developer_tools/pki/, git-ignored):
#   rootCA.crt / rootCA.key             the dev CA; trust rootCA.crt in your browser
#   tls_<service>.crt / tls_<service>.key   server certificate and key
#   tls_<service>_chain.pem             certificate followed by the CA
#
# Existing files are kept, so the CA stays the same across runs. Delete the
# directory to start over. NEVER use these in production.
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PKI_DIR="${SCRIPT_DIR}/../pki"
mkdir -p "${PKI_DIR}"
cd "${PKI_DIR}"

# Services that serve TLS, with their fixed IP on the compose network
# (sisnet, 172.28.0.0/16). Every certificate also covers localhost and
# 127.0.0.1, because the ports are published on the host's loopback.
service_names="sis mellon"
service_ip() {
    case "$1" in
        sis)    echo "172.28.0.10" ;;
        mellon) echo "172.28.0.11" ;;
        *)      echo "" ;;
    esac
}

dn() {
    cat <<EOF
[req]
prompt             = no
default_md         = sha256
distinguished_name = dn

[dn]
C  = SE
ST = Milky Way
L  = Earth
O  = SUNET
OU = sis_dev
CN = $1
EOF
}

if [ ! -f rootCA.key ]; then
    echo "Creating root CA"
    dn "sis_dev_ca" > ca.conf
    openssl genrsa -out rootCA.key 2048 2>/dev/null
    openssl req -x509 -new -nodes -key rootCA.key -sha256 -days 3650 \
        -out rootCA.crt -config ca.conf \
        -addext "basicConstraints=critical,CA:TRUE" \
        -addext "keyUsage=critical,keyCertSign,cRLSign"
    rm -f ca.conf
fi

create_leaf_cert() {
    name=$1
    [ -f "tls_${name}.key" ] && return 0
    echo "Creating TLS certificate for ${name}"
    dn "${name}" > "${name}.conf"
    cat > "${name}.ext" <<EOF
authorityKeyIdentifier = keyid,issuer
basicConstraints       = CA:FALSE
keyUsage               = critical, digitalSignature, keyEncipherment
extendedKeyUsage       = serverAuth
subjectAltName         = @alt_names

[alt_names]
DNS.1 = ${name}
DNS.2 = localhost
IP.1  = 127.0.0.1
EOF
    ip=$(service_ip "${name}")
    [ -n "${ip}" ] && echo "IP.2  = ${ip}" >> "${name}.ext"

    openssl req -new -nodes -newkey rsa:2048 -keyout "tls_${name}.key" \
        -out "${name}.csr" -config "${name}.conf" 2>/dev/null
    # Browsers reject server certificates valid for more than 398 days.
    openssl x509 -req -in "${name}.csr" -CA rootCA.crt -CAkey rootCA.key \
        -CAcreateserial -out "tls_${name}.crt" -days 397 -sha256 \
        -extfile "${name}.ext" 2>/dev/null
    cat "tls_${name}.crt" rootCA.crt > "tls_${name}_chain.pem"
    # Readable by the non-root users the containers run as. Dev keys only.
    chmod 644 "tls_${name}.key"
    rm -f "${name}.csr" "${name}.conf" "${name}.ext"
}

for name in ${service_names}; do
    create_leaf_cert "${name}"
done
rm -f rootCA.srl

echo "PKI ready in $(cd "${PKI_DIR}" && pwd). Trust rootCA.crt in your browser."
