bbalancer:
	docker build -t shieldxbot/balancer:v0.1.26 -f Baseline/RoundRobin/Balancer/Dockerfile Baseline/RoundRobin/Balancer
	docker push shieldxbot/balancer:v0.1.26



bbclient: 
	docker build -t shieldxbot/client:v0.1.26 -f  Baseline/RoundRobin/Client/Dockerfile Baseline/RoundRobin/Client
	docker push shieldxbot/client:v0.1.26


bbackend: 
	docker build -t shieldxbot/backend:v0.1.26 -f Baseline/RoundRobin/Backend/Dockerfile Baseline/RoundRobin/Backend
	docker push shieldxbot/backend:v0.1.26

delete:
	kubectl delete -f kubernetes/Deployment/Balancer/Deployment.yaml
	kubectl delete -f kubernetes/Deployment/Client/Deployment.yaml
	kubectl delete -f kubernetes/Deployment/Backend/Deployment.yaml
	
 


apply:
	kubectl apply -f kubernetes/Deployment/Balancer/Deployment.yaml
	kubectl apply -f kubernetes/Deployment/Backend/Deployment.yaml


apply-client:
	kubectl apply -f kubernetes/Deployment/Client/Deployment.yaml



