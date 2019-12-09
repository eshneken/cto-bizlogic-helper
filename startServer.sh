#!/bin/bash

sudo go build main.go
nohup sudo -E ./cto-bizlogic-helper > ~/server.out &