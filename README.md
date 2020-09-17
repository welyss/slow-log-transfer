# SlowLogTransfer
 SlowLogTransfer
mysql慢查询迁移,从mysql的mysql.slow_log表定时导出数据到elasticsearch,并在grafana中展示

config.yaml
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
```
![image1](https://github.com/welyss/slow-log-transfer/blob/master/mysqlslowlog-grafana-dashboard1.png?raw=true)
![image2](https://github.com/welyss/slow-log-transfer/blob/master/mysqlslowlog-grafana-dashboard2.png?raw=true)
![image3](https://github.com/welyss/slow-log-transfer/blob/master/mysqlslowlog-grafana-dashboard3.png?raw=true)
