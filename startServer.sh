#!/bin/bash

cd /home/opc/cto-bizlogic-helper/
go build
export TNS_ADMIN=/home/opc/wallet
nohup ./cto-bizlogic-helper >& /home/opc/server.out &
