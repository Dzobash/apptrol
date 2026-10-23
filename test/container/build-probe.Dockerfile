FROM docker.io/library/ubuntu:26.04
# Only this trusted probe sees the host cgroup namespace, to verify ancestry.
# Recipe builds retain a private cgroup namespace (QA-16, ADR 0037).
ARG EXPECTED_SCOPE
COPY build-limits.sh /build-limits.sh
RUN /bin/sh /build-limits.sh
RUN /bin/sh /build-limits.sh
