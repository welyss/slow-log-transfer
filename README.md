# SlowLogTransfer
 SlowLogTransfer
mysql慢查询迁移,从mysql的mysql.slow_log表定时导出数据到elasticsearch,并在grafana中展示
```
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
