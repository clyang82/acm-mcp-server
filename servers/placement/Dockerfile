FROM golang:1.25-bookworm AS builder

WORKDIR /go/src/github.com/stolostron/placement-mcp
COPY . .

RUN CGO_ENABLED=0 go build -a -o placement-mcp .

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
ENV USER_UID=10001

COPY --from=builder /go/src/github.com/stolostron/placement-mcp/placement-mcp /

USER ${USER_UID}

ENTRYPOINT ["/placement-mcp"]