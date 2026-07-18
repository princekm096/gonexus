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

EXPOSE 8080
ENTRYPOINT ["gonexus"]
CMD ["serve", ":8080"]
