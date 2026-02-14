# Deployment Guide: Ubuntu EC2

## Overview

The app is deployed as a systemd service on an Ubuntu EC2 instance. The binary is built
directly on the server from source and runs on port 3000.

## Prerequisites

- An EC2 instance running Ubuntu (t2.micro or larger)
- SSH access via .pem key file
- Port 3000 open in the EC2 security group (see AWS Console step below)

## AWS Console: Open Port 3000

In the EC2 Console:
1. Go to **Security Groups** for your instance
2. Edit **Inbound Rules**
3. Add rule: Type = Custom TCP, Port = 3000, Source = 0.0.0.0/0
4. Save

## Server Setup (run over SSH)

```bash
ssh -i your-key.pem ubuntu@<ec2-public-ip>
```

### 1. Install dependencies

```bash
sudo apt update && sudo apt install -y git
```

### 2. Install Go 1.24

```bash
wget https://go.dev/dl/go1.24.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.24.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
source ~/.profile
go version
```

### 3. Clone the repo

```bash
git clone https://github.com/timLP79/cs408-go-stack.git go-full-stack
cd go-full-stack
```

### 4. Build the binary

```bash
go mod download
go build -o go-full-stack .
```

### 5. Install and start the service

```bash
sudo cp deploy/go-full-stack.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable go-full-stack
sudo systemctl start go-full-stack
sudo systemctl status go-full-stack
```

## Verification

```bash
# Check service is running
sudo systemctl status go-full-stack

# Test locally on the server
curl http://localhost:3000
```

Then visit `http://<ec2-public-ip>:3000` in your browser.

## Useful Commands

```bash
# View live logs
journalctl -u go-full-stack -f

# Restart after a code update
git pull
go build -o go-full-stack .
sudo systemctl restart go-full-stack
```
