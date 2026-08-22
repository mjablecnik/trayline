FROM alpine:3.20

RUN apk add --no-cache bash curl \
    && CHISEL_VERSION="1.9.1" \
    && curl -fsSL "https://github.com/jpillora/chisel/releases/download/v${CHISEL_VERSION}/chisel_${CHISEL_VERSION}_linux_amd64.gz" | gunzip > /usr/local/bin/chisel \
    && chmod +x /usr/local/bin/chisel \
    && rm -rf /var/cache/apk/*

WORKDIR /app

COPY entrypoint.sh ./
RUN chmod +x entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
