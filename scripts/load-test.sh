#!/bin/bash

export WORKSPACE=$(pwd)/rinha-original/load-test

./rinha-original/gatling-charts-highcharts-bundle-3.9.5/bin/gatling.sh -rm local -s RinhaBackendCrebitosSimulation -rd "DESCRICAO" -rf $WORKSPACE/user-files/results -sf $WORKSPACE/user-files/simulations -rsf $WORKSPACE/user-files/resources