FROM golang:alpine AS builder
ENV GOOS=linux \
    GOARCH=amd64 
WORKDIR /build

COPY web_cache_server/ ./web_cache_server/
COPY jnlee_project.go go.mod ./

RUN go mod download
RUN go build -o jnlee_project .

FROM ubuntu:20.04
WORKDIR /build

COPY --from=builder /build/ .

CMD ["./jnlee_project"]
