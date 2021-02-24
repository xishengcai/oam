FROM registry.cn-hangzhou.aliyuncs.com/launcher/alpine:latest
COPY ./bin/oam /
WORKDIR /
ENTRYPOINT ["/oam"]
