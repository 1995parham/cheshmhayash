# cheshmhayash
[![Drone (cloud)](https://img.shields.io/drone/build/1995parham/cheshmhayash.svg?style=flat-square)](https://cloud.drone.io/1995parham/cheshmhayash)

## Introduction
[NATS Messaging System](https://nats.io/) report its metrics and state over its monitoring endpoint. This project aims to visulize and proxy these metrics.
For example your can deploy this project with following architecture and transprant your servers from users:

```
 o
-|-   --- cheshmhayash -- NATS
 /\
```

Each NATS server reports its status only and this project can aggregate theses metrics for you.
