FROM alpine:3.20

RUN apk add --no-cache \
    bash \
    wireguard-tools \
    iptables \
    ip6tables \
    iproute2 \
    socat \
    && rm -rf /var/cache/apk/*

WORKDIR /app

COPY entrypoint.sh health.sh ./
RUN chmod +x entrypoint.sh health.sh

ENTRYPOINT ["/app/entrypoint.sh"]
