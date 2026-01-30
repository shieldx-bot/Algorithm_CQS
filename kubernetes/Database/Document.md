## kubectl get pvc -n postgre-db 

## kubectl delete pvc -n postgre-db postgres-pvc


## kubectl describe pod -n postgre-db postgres-0


##  kubectl -n postgre-db port-forward svc/redis 6379:6379


## ##  kubectl -n client-ns port-forward svc/client 3000:3000


## kubectl -n monitoring port-forward svc/monitoring-grafana 3001:80


## kubectl -n monitoring delete pod prometheus-monitoring-kube-prometheus-prometheus-0

kubectl delete pod prometheus-monitoring-kube-prometheus-prometheus-0 -n monitoring --grace-period=0 --force



## cách export google cloud redis về  laptop 
