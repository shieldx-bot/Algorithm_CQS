bbalancer:
	docker build -t shieldxbot/balancer:v0.1.37 -f ./Baseline/ResourceBased/Balancer/Dockerfile ./Baseline/ResourceBased/Balancer
	docker push shieldxbot/balancer:v0.1.37



bbclient: 
	docker build -t shieldxbot/client:v0.1.37 -f  ./Baseline/ResourceBased/Client/Dockerfile ./Baseline/ResourceBased/Client
	docker push shieldxbot/client:v0.1.37


bbackend: 
	docker build -t shieldxbot/backend:v0.1.37 -f ./Baseline/ResourceBased/Backend/Dockerfile ./Baseline/ResourceBased/Backend
	docker push shieldxbot/backend:v0.1.37

 


delete:
	kubectl delete -f kubernetes/Deployment/Balancer/Deployment.yaml
	kubectl delete -f kubernetes/Deployment/Client/Deployment.yaml
	kubectl delete -f kubernetes/Deployment/Backend/Deployment.yaml
	
 


apply:
	kubectl apply -f kubernetes/Deployment/Balancer/Deployment.yaml
	kubectl apply -f kubernetes/Deployment/Backend/Deployment.yaml


apply-client:
	kubectl apply -f kubernetes/Deployment/Client/Deployment.yaml



  