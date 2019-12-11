#!/bin/bash

cd /home/opc/cto-bizlogic-helper/
sudo go build
nohup sudo -E ./cto-bizlogic-helper > ~/server.out &
