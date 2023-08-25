FROM golang:1.21-alpine3.18

COPY ./ /
RUN cd / && go build
RUN apk add curl

ENV CGO_ENABLED=0
ENTRYPOINT ["/consensuswarn"]
