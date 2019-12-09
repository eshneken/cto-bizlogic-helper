#!/bin/bash

sudo go build
nohup sudo -E ./cto-bizlogic-helper > ~/server.out &