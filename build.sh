#!/bin/bash

export GOPROXY=https://goproxy.cn

BIN_FILE="lsh-mcp-lcs-timer"

while [ $# -gt 0 ]
do
    key="$1"
    case $key in
        --image-name)
            export IMAGE_NAME=$2
            shift
        ;;
        --version)
            export VERSION=$2
            shift
        ;;
        *)
            echo "unknown option [$key]"
            exit 1
        ;;
    esac
    shift
done

if [ -z "$IMAGE_NAME" ]; then
  IMAGE_NAME='lsh-mcp-lcs-timer'
fi

if [ -z "$VERSION" ]; then
  VERSION='dev'
fi

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build   -o  ./bin/${BIN_FILE} ./main.go

if [ $? -ne 0 ]; then
    echo "build ERROR"
    exit 1
fi
echo build success

cat <<EOF > Dockerfile
FROM registry.cn-hangzhou.aliyuncs.com/launcher/alpine:latest

MAINTAINER xishengcai <cc710917049@163.com>

COPY ./bin/${BIN_FILE} /usr/local/bin
COPY ./yaml /opt/yaml

RUN chmod +x /usr/local/bin/${BIN_FILE}

WORKDIR /opt

CMD ["${BIN_FILE}"]
EOF
#
docker build -t registry.cn-beijing.aliyuncs.com/xlauncher-dev/${IMAGE_NAME}:${VERSION} ./
docker push    registry.cn-beijing.aliyuncs.com/xlauncher-dev/${IMAGE_NAME}:${VERSION}
docker rmi     registry.cn-beijing.aliyuncs.com/xlauncher-dev/${IMAGE_NAME}:${VERSION}
