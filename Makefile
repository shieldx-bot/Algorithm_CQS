bbalancer:
	docker build -t shieldxbot/balancer:v0.0.2 -f CQS/balancer/Dockerfile CQS/balancer
    docker push shieldxbot/balancer:v0.0.2



bbclient: 
	docker build -t shieldxbot/client:v0.0.3 -f CQS/client/Dockerfile CQS/client
	docker push shieldxbot/client:v0.0.3


bbackend: 
	docker build -t shieldxbot/backend:v0.0.3 -f CQS/backend/Dockerfile CQS/backend
	docker push shieldxbot/backend:v0.0.3