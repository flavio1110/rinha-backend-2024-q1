FROM alpine:3.15.0
RUN apk add --update curl && rm -rf /var/cache/apk/*
COPY bin/rinha-backend-2024-q1 /rinha-backend-2024-q1

EXPOSE 9999
ENTRYPOINT /rinha-backend-2024-q1

