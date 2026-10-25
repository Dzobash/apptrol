FROM docker.io/library/ubuntu@sha256:68ee22004a9109b44973313b02ff7b1de250f1de0f5ecb628d73f579aaff33ac
# Only this trusted probe sees the host cgroup namespace, to verify ancestry.
# Recipe builds retain a private cgroup namespace (QA-16, ADR 0037).
ARG EXPECTED_SCOPE
COPY build-limits.sh /build-limits.sh
RUN /bin/sh /build-limits.sh
RUN /bin/sh /build-limits.sh
