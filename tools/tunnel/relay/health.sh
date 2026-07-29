#!/bin/sh

# Check if chisel process is running
if pgrep -x chisel > /dev/null 2>&1; then
    status="running"
    http_status="200 OK"
else
    status="stopped"
    http_status="503 Service Unavailable"
fi

json="{\"chisel\": \"${status}\"}"
content_length=${#json}

printf "HTTP/1.1 %s\r\n" "$http_status"
printf "Content-Type: application/json\r\n"
printf "Content-Length: %d\r\n" "$content_length"
printf "Connection: close\r\n"
printf "\r\n"
printf "%s" "$json"
