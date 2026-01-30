bbalancer:
	docker build -t shieldxbot/balancer:v0.1.16 -f QL/balancer/Dockerfile QL/balancer
    docker push shieldxbot/balancer:v0.1.16



bbclient: 
	docker build -t shieldxbot/client:v0.1.16 -f QL/client/Dockerfile QL/client
	docker push shieldxbot/client:v0.1.16


bbackend: 
	docker build -t shieldxbot/backend:v0.1.16 -f QL/backend/Dockerfile QL/backend
	docker push shieldxbot/backend:v0.1.16

delete:
	kubectl delete -f kubernetes/Deployment/Balancer/Deployment.yaml
	kubectl delete -f kubernetes/Deployment/Client/Deployment.yaml
	kubectl delete -f kubernetes/Deployment/Backend/Deployment.yaml
	
 


apply:
	kubectl apply -f kubernetes/Deployment/Balancer/Deployment.yaml
	kubectl apply -f kubernetes/Deployment/Backend/Deployment.yaml


apply-client:
	kubectl apply -f kubernetes/Deployment/Client/Deployment.yaml



