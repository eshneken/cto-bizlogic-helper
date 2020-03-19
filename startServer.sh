#!/bin/bash

cd /home/opc/cto-bizlogic-helper/
go build
sudo setcap CAP_NET_BIND_SERVICE=+eip /home/opc/cto-bizlogic-helper/cto-bizlogic-helper
export TNS_ADMIN=/home/opc/wallet
nohup ./cto-bizlogic-helper >& /home/opc/server.out &
