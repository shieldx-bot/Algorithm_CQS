bbalancer:
	docker build -t shieldxbot/balancer:v0.1.26 -f Baseline/RL/Balancer/Dockerfile Baseline/RL/Balancer
	docker push shieldxbot/balancer:v0.1.26



bbclient: 
	docker build -t shieldxbot/client:v0.1.26 -f  Baseline/RL/Client/Dockerfile Baseline/RL/Client
	docker push shieldxbot/client:v0.1.26


bbackend: 
	docker build -t shieldxbot/backend:v0.1.26 -f Baseline/RL/Backend/Dockerfile Baseline/RL/Backend
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



