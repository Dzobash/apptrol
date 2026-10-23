FROM docker.io/library/ubuntu@sha256:067402f52dc34dacdf334d215eb58e7e252eb82df9e2c96e2d81a8af13a70fb4
# Only this trusted probe sees the host cgroup namespace, to verify ancestry.
# Recipe builds retain a private cgroup namespace (QA-16, ADR 0037).
ARG EXPECTED_SCOPE
COPY build-limits.sh /build-limits.sh
RUN /bin/sh /build-limits.sh
RUN /bin/sh /build-limits.sh
