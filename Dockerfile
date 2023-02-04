FROM golang:1.19-alpine as builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY *.go ./

RUN go build -o /kubernetes-resource-replicator

CMD [ "/kubernetes-resource-replicator" ]
