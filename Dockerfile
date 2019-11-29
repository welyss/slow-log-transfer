FROM alpine

COPY slow-log-transfer /usr/local/bin/
COPY healthcheck.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/slow-log-transfer
RUN chmod +x /usr/local/bin/healthcheck.sh
COPY Shanghai /etc/localtime

ENTRYPOINT ["slow-log-transfer"]