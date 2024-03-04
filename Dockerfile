FROM gcr.io/distroless/static:nonroot
WORKDIR /
#go build -a -ldflags "-extldflags '-static'" -o ./manager .
COPY manager .
# Use uid of nonroot user (65532) because kubernetes expects numeric user when applying PSPs
USER 65532
ENTRYPOINT ["/manager"]