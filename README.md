# ZLI

A command line tool used for deploying Jenkins job directly from the terminal.

There isn't a central list of site alias to Jenkins job mapping yet, 
the current workaround would be adding the frequently deployed sites to your 
configuration file.

## Installation
- Install Go from [the official developer](https://go.dev/doc/install)
- Clone this repository and `cd` into it
- Run `go install`
  - Go might place your executable under $GOPATH, if so, run `go env GOPATH` and then `sudo cp /path/of/GOENV/bin/zli /usr/local/`
- Run `zli` and follow the set up process

### Building the project yourself
- Clone the repository
- `cd` and run `go build zli.go`
- You can then manage the executable yourself, `mv zli /usr/bin/zli` for example

## Limitations
- At the moment only boolean parameters can be customised directly on the terminal
