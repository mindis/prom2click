FROM golang:1.9.2-alpine AS build
RUN apk update
RUN apk add git
RUN mkdir -p /go/src/github.com/mindis/prom2click
ADD . /go/src/github.com/mindis/prom2click
RUN go get -v github.com/Masterminds/glide
WORKDIR /go/src/github.com/mindis/prom2click 
RUN apk add make
RUN make get-deps
RUN go get -v github.com/mindis/prom2click
FROM alpine:3.5
COPY --from=build /go/bin/prom2click /usr/local/bin/prom2click
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /usr/local/go/lib/time/zoneinfo.zip
ENTRYPOINT ["/usr/local/bin/prom2click"]