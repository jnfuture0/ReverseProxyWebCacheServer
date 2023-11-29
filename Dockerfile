FROM golang:alpine AS builder
ENV GOOS=linux \
    GOARCH=amd64 
WORKDIR /build

COPY wcs/ ./wcs/
COPY workerpool/ ./workerpool/
COPY cache/ ./cache/
COPY jnlee.go go.mod go.sum ./

RUN go mod download
RUN go build -o jnlee .

FROM ubuntu:20.04
WORKDIR /build

COPY --from=builder /build/ .

CMD ["./jnlee"]
