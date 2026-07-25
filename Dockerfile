# GoNexus image. Indexing Go repos needs the Go toolchain at runtime, and
# TS/Vue indexing needs Node + the extractor deps — so the runtime is the Go
# image with Node added, not a scratch/distroless base.
FROM golang:1.25-bookworm

# Node for the TS/Vue extractor.
RUN apt-get update && apt-get install -y --no-install-recommends nodejs npm git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /usr/local/bin/gonexus ./cmd/gonexus

# Extractor deps.
RUN cd tools/ts-extractor && npm install --omit=dev
ENV GONEXUS_TS_EXTRACTOR=/src/tools/ts-extractor/extract.mjs

# Run as a non-root user (least privilege).
RUN useradd --create-home --uid 10001 gonexus \
    && chown -R gonexus:gonexus /src
USER gonexus

# In a container the process is meant to be reachable, so bind all interfaces.
# The server FAILS TO START on a non-loopback bind without GONEXUS_AUTH_TOKEN,
# because the API can execute the build toolchain (Index) and read your source
# graph. Run with a token, e.g.:
#   docker run -e GONEXUS_AUTH_TOKEN=$(openssl rand -hex 32) [-e GONEXUS_READ_ONLY=1] ...
EXPOSE 8080
ENTRYPOINT ["gonexus"]
CMD ["serve", "0.0.0.0:8080"]
