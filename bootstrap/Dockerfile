FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bootstrap .

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/bootstrap /bootstrap

ENTRYPOINT ["/bootstrap"]
