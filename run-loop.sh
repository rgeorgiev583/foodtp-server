#!/bin/bash

trap "trap '' SIGCHLD && killall $1 && exit" SIGINT
trap "$* &" SIGCHLD

$* &

while :; do
    :
done