FROM golang:1.22-bookworm AS build
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=1
RUN go build -trimpath -ldflags "-s -w" -o /out/motocam ./cmd/motocam

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/motocam /usr/local/bin/motocam
ENV ADDR=:8787
ENV DATA_DIR=/data
EXPOSE 8787
VOLUME ["/data"]
ENTRYPOINT ["motocam"]
