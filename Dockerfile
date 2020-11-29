FROM golang:1.13-alpine as builder

WORKDIR /go/src/app
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build cmd/coral_edgetpu/coral_edgetpu.go

FROM alpine
COPY --from=builder /go/src/app/coral_edgetpu .

CMD ["./coral_edgetpu", "-logtostderr"]
