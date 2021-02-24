#!/bin/bash

export GOPROXY=https://goproxy.cn

BIN_FILE="oam"

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
        --repo)
            export IMAGE_REPO=$2
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
  IMAGE_NAME='oam'
fi

if [ -z "$VERSION" ]; then
  VERSION='dev'
fi

if [ -z "$IMAGE_REPO" ]; then
  IMAGE_REPO='registry.cn-beijing.aliyuncs.com/xlauncher-dev'
fi

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build   -o  ./bin/${BIN_FILE} ./main.go

if [ $? -ne 0 ]; then
    echo "compile ERROR"
    exit 1
fi
echo build success

cat <<EOF > Dockerfile
FROM registry.cn-hangzhou.aliyuncs.com/launcher/alpine:latest
COPY ./bin/${BIN_FILE} /
WORKDIR /
ENTRYPOINT ["/${BIN_FILE}"]
EOF

if [ $? -ne 0 ]; then
    echo "build image ERROR"
    exit 1
fi
docker build -t ${IMAGE_REPO}/${IMAGE_NAME}:${VERSION} ./
docker push    ${IMAGE_REPO}/${IMAGE_NAME}:${VERSION}
docker rmi     ${IMAGE_REPO}/${IMAGE_NAME}:${VERSION}
