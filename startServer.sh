#!/bin/bash

cd /home/opc/cto-bizlogic-helper/
sudo go build
export TNS_ADMIN=/home/opc/wallet
nohup sudo -E ./cto-bizlogic-helper >& /home/opc/server.out &
