bbalancer:
	docker build -t shieldxbot/balancer:v0.0.6 -f CQS/balancer/Dockerfile CQS/balancer  
    docker push shieldxbot/balancer:v0.0.6



bbclient: 
	docker build -t shieldxbot/client:v0.0.6 -f CQS/client/Dockerfile CQS/client
	docker push shieldxbot/client:v0.0.6


bbackend: 
	docker build -t shieldxbot/backend:v0.0.6 -f CQS/backend/Dockerfile CQS/backend
	docker push shieldxbot/backend:v0.0.6

delete:
	kubectl delete -f kubernetes/Deployment/Backend/Deployment.yaml
	kubectl delete -f kubernetes/Deployment/Balancer/Deployment.yaml
	kubectl delete -f kubernetes/Deployment/Client/Deployment.yaml


apply:
	kubectl apply -f kubernetes/Deployment/Backend/Deployment.yaml
	kubectl apply -f kubernetes/Deployment/Balancer/Deployment.yaml
	kubectl apply -f kubernetes/Deployment/Client/Deployment.yaml



