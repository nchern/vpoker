FROM base-builder:latest AS builder

WORKDIR /go/src/github.com/nchern/vpoker
COPY . .

RUN make install-deps install

FROM alpine:3.19

WORKDIR /

COPY --from=builder /go/bin/vpoker /bin/

RUN mkdir /www

COPY ./web/ /www/web

# main service port
EXPOSE 8080

# metrics exposing port
EXPOSE 49100

WORKDIR /www
CMD /bin/vpoker
