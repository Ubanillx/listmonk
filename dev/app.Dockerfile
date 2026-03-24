FROM golang:1.26.1 AS go

FROM node:20 AS node

COPY --from=go /usr/local/go /usr/local/go
ENV GOPATH /go
ENV CGO_ENABLED=0
ENV GOPROXY https://goproxy.cn,direct
ENV GOSUMDB sum.golang.google.cn
ENV npm_config_registry https://registry.npmmirror.com
ENV YARN_REGISTRY https://registry.npmmirror.com
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH

WORKDIR /app
CMD [ "sleep infinity" ]
