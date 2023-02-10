IMAGE_NAME=example-webrtc
PARENT_DIR := `pwd`/../

docker/build:
	docker build -t ${IMAGE_NAME} .

docker/run:
	docker run \
	--net=host \
	-v ${PARENT_DIR}:/Work \
	-it ${IMAGE_NAME} /bin/bash

hoge:
	echo ${PARENT_DIR}
