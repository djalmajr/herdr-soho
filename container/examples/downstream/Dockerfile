# Example downstream image built FROM the herdr-soho base.
#
# Local tag (the base was built and loaded on this Docker engine; `docker build`
# uses the engine's own builder, which resolves local images):
#   docker build -t my-job-image:dev container/examples/downstream
#
# Registry digest (after the base is pushed to a registry you control; the
# digest is the RepoDigest the push prints, not the local image ID):
#   docker build --build-arg BASE_IMAGE=<registry>/herdr-soho-base:0.1.0@sha256:<repo digest> ...
ARG BASE_IMAGE=herdr-soho-base:0.1.0
FROM ${BASE_IMAGE}

# Project-specific additions go here. Keep credentials out of the build:
# no ARG or ENV carries a key, and nothing is copied from a home directory.
USER root
RUN mkdir -p /opt/example && herdr-soho --version > /opt/example/base-cli-version
USER agent
