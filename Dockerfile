FROM golang:1.14 as builder

WORKDIR /root/go/src/oam
COPY ./ ./
ENV GOPROXY="https://goproxy.io"
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o controller main.go

FROM alpine:latest
WORKDIR /
COPY --from=builder /root/go/src/oam/controller .
USER nonroot:nonroot

ENTRYPOINT ["/controller"]
