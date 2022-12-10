FROM golang:1.19-alpine3.15

COPY ./ /
RUN cd / && go build
RUN apk add curl

ENV CGO_ENABLED=0
ENTRYPOINT ["/consensuswarn"]
