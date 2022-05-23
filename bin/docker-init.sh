#!/bin/bash

echo "Initializing."

if [[ -z "$( which nc )" && -n "$( which apt-get )" ]]; then
    apt-get update
    apt-get install -y netcat
fi

while [[ true ]]; do
    if nc -z db 3306; then
        echo "db is up."
        break;
    fi;
    echo "Waiting for db..."
    sleep 1;
done

wp core install \
    --url="http://wordpress" \
    --title="cron-control-runner WordPress Tester" \
    --admin_user="admin" \
    --admin_email="example@example.com";

/usr/local/bin/wp-cron-runner \
    -debug=true \
    -token="$WP_CLI_TOKEN" \
    -events-webhook-url="$WP_CLI_EVENTS_WEBHOOK_URL" \
    -use-websockets=true \
    -wp-cli-path=/usr/local/bin/wp \
    -wp-path=/var/www/html
