

.PHONY: default run stop clean

default:
	docker build -t gpt-kook:dev .

run:
	docker compose up -d

stop:
	docker compose down

clean:
	docker compose down -v --rmi all
