FROM debian:buster-slim

RUN apt update \
	&& apt install -y curl gnupg2 git python3 python3-pip python3-numpy python3-pillow zlib1g-dev libjpeg-dev --no-install-recommends \
	&& echo "deb https://packages.cloud.google.com/apt coral-edgetpu-stable main" | tee /etc/apt/sources.list.d/coral-edgetpu.list \
	&& curl https://packages.cloud.google.com/apt/doc/apt-key.gpg |  apt-key add - \
	&& apt update \
	&& apt install -y libedgetpu1-std \
	&& pip3 install https://github.com/google-coral/pycoral/releases/download/release-frogfish/tflite_runtime-2.5.0-cp37-cp37m-linux_x86_64.whl \
	&& git clone https://github.com/google-coral/tflite.git \
	&& ./tflite/python/examples/classification/install_requirements.sh \
	&& ./tflite/python/examples/detection/install_requirements.sh

WORKDIR /tflite/python/examples/
ENTRYPOINT ["python3"]
CMD ["classification/classify_image.py", \
	"--model", "classification/models/mobilenet_v2_1.0_224_inat_bird_quant_edgetpu.tflite", \
	"--labels", "classification/models/inat_bird_labels.txt", \
	"--input", "classification/images/parrot.jpg"]
