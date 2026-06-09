# jsonstrict

Go package that unmarshals JSON like `encoding/json`, but also reports unknown
fields. Unknown fields never cause an error — callers decide what to do.

## Install

```
go get github.com/13rac1/jsonstrict
```

## Usage

```go
var config Config
unknownFields, err := jsonstrict.Unmarshal(data, &config)
if err != nil {
    return err
}
if len(unknownFields) > 0 {
    log.Printf("unexpected fields: %v", unknownFields)
}
```

`v` must be a non-nil pointer to a struct.

## License

Apache 2.0
