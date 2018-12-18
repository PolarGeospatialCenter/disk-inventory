FROM golang:stretch

WORKDIR /go/src/github.com/PolarGeospatialCenter/disk-inventory
RUN curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
RUN apt-get update && apt-get install -y libudev-dev

COPY Gopkg.lock Gopkg.toml ./
RUN dep ensure -vendor-only -v 
COPY ./ .
RUN go build -o /bin/disk-inventory ./cmd/disk-inventory

FROM debian:stretch-slim
COPY --from=0 /bin/disk-inventory /bin/disk-inventory
ENTRYPOINT /bin/disk-inventory
