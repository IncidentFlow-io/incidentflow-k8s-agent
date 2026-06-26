FROM golang:1.25-alpine AS builder

WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG VERSION=0.1.0
ARG COMMIT=dev
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X github.com/incidentflow/incidentflow-k8s-agent/internal/version.Version=${VERSION} -X github.com/incidentflow/incidentflow-k8s-agent/internal/version.Commit=${COMMIT}" \
    -o /out/incidentflow-k8s-agent ./cmd/agent

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/incidentflow-k8s-agent /incidentflow-k8s-agent
USER nonroot:nonroot
ENTRYPOINT ["/incidentflow-k8s-agent"]
