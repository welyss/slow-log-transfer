# SlowLogTransfer
 SlowLogTransfer

============================ config.yaml ============================
tasks:
-  instance: wystest1
   index: mysql_slowlogs
   mysql: user:password@/(localhost)/schema?readTimeout=30s&charset=utf8
   es:
   - http://10.0.29.73:9200
   - http://10.0.29.75:9200
   - http://10.0.29.117:9200
   eviction: true
   interval: 30