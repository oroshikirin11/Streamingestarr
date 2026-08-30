FROM golang:alpine AS build

RUN apk update && apk add --no-cache git gcc build-base linux-headers

WORKDIR /build
COPY . /build

ARG VERSION=dev
ENV VERSION=${VERSION}
ARG GIT_COMMIT
ENV GIT_COMMIT=${GIT_COMMIT}
ARG NAME=docker
ENV NAME=${NAME}

RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -ldflags "-extldflags \"-static\" -s -w -X streamingestarr/config.GitCommit=$GIT_COMMIT -X streamingestarr/config.VersionNumber=$VERSION -X streamingestarr/config.BuildPlatform=$NAME" -o streamingestarr .

# Create the image by copying the result of the build into a new alpine image
FROM alpine:3.23.3
RUN apk update && apk add --no-cache ffmpeg ffmpeg-libs ca-certificates && update-ca-certificates

RUN addgroup -g 101 -S streamingestarr && adduser -u 101 -S streamingestarr -G streamingestarr

# Copy app assets
WORKDIR /app
COPY --from=build /build/streamingestarr /app/streamingestarr
RUN mkdir /app/data
RUN chown -R streamingestarr:streamingestarr /app
USER streamingestarr
ENTRYPOINT ["/app/streamingestarr"]
EXPOSE 8080 1935 9710/udp
