#!/usr/bin/env sh
set -eu

OPSTACK_DOCTOR_BIN="${OPSTACK_DOCTOR_BIN:-/usr/local/bin/opstack-doctor}"
OPSTACK_DOCTOR_CONFIG="${OPSTACK_DOCTOR_CONFIG:-/etc/opstack-doctor/doctor.yaml}"
TEXTFILE_DIR="${TEXTFILE_DIR:-/var/lib/node_exporter/textfile_collector}"
OUTPUT_FILE="${OUTPUT_FILE:-${TEXTFILE_DIR}/opstack_doctor.prom}"

tmp_file="$(mktemp "${OUTPUT_FILE}.tmp.XXXXXX")"
cleanup() {
  rm -f "${tmp_file}"
}
trap cleanup EXIT

"${OPSTACK_DOCTOR_BIN}" export metrics --config "${OPSTACK_DOCTOR_CONFIG}" > "${tmp_file}"
chmod 0644 "${tmp_file}"
mv "${tmp_file}" "${OUTPUT_FILE}"
trap - EXIT
