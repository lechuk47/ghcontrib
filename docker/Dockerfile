# syntax=docker/dockerfile:1.0-experimental
FROM golang:buster AS build
COPY . /app
WORKDIR /app
RUN go build /app

FROM ubuntu:20.04
# IT shuold be better to get root ca certificates installing the updated ca-certificates package
# This is going extremely slow to just copy one file.
COPY --from=build /app/ghcontrib /app/docker/ca-certificates.crt /app/
RUN mkdir -p /etc/ssl/certs/ && mv /app/ca-certificates.crt /etc/ssl/certs/
WORKDIR /app
CMD [ "/app/ghcontrib"]