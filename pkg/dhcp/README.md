After changing definitions in configuration.proto please remember to regenerate
configuration.pb.go using:

```sh
  protoc --go_out=plugins=grpc:. configuration.proto
```
