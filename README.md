Screen ruler running on x window system.

![](demo.gif)

Inspired by [swillner/highlight-pointer](https://github.com/swillner/highlight-pointer).

## Run

```shell
$ go run .
```

If you want to make the ruler transparent, you need to run compton!

```shell
$ apt install compton
$ compton
```

## Debug

```shell
$ go build . && xtrace -o output.log ./xpoint
```
