/*
Copyright 2017 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

const (
	defaultCirrosLocation = "github.com/mirantis/virtlet/releases/download/v0.8.2/cirros.img"

	sshPublicKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCaJEcFDXEK2ZbX0ZLS1EIYFZRbDAcRfuVjpstSc0De8+sV1aiu+deP" +
		"xdkuDRwqFtCyk6dEZkssjOkBXtri00MECLkir6FcH3kKOJtbJ6vy3uaJc9w1ERo+wyl6SkAh/+JTJkp7QRXj8oylW5E20LsbnA/dIwW" +
		"zAF51PPwF7A7FtNg9DnwPqMkxFo1Th/buOMKbP5ZA1mmNNtmzbMpMfJATvVyiv3ccsSJKOiyQr6UG+j7sc/7jMVz5Xk34Vd0l8GwcB0" +
		"334MchHckmqDB142h/NCWTr8oLakDNvkfC1YneAfAO41hDkUbxPtVBG5M/o7P4fxoqiHEX+ZLfRxDtHB53 me@localhost"

	sshPrivateKey = `
		-----BEGIN RSA PRIVATE KEY-----
		MIIEpAIBAAKCAQEAmiRHBQ1xCtmW19GS0tRCGBWUWwwHEX7lY6bLUnNA3vPrFdWo
		rvnXj8XZLg0cKhbQspOnRGZLLIzpAV7a4tNDBAi5Iq+hXB95CjibWyer8t7miXPc
		NREaPsMpekpAIf/iUyZKe0EV4/KMpVuRNtC7G5wP3SMFswBedTz8BewOxbTYPQ58
		D6jJMRaNU4f27jjCmz+WQNZpjTbZs2zKTHyQE71cor93HLEiSjoskK+lBvo+7HP+
		4zFc+V5N+FXdJfBsHAdN9+DHIR3JJqgwdeNofzQlk6/KC2pAzb5HwtWJ3gHwDuNY
		Q5FG8T7VQRuTP6Oz+H8aKohxF/mS30cQ7RwedwIDAQABAoIBABAZa+WGMuFcOpoO
		BJTKoKCdWGJuDirwowrWd/QDn6nptgsQxs6Hv9D/bCCYM/HdcizEqTrGqGFd0lRX
		UOtR/3TjaFrMF0Fk9CJyKR/LM/Vo/JEsrbpJMAGQJrvkF3C1pjDjFfJrqNqnEbOP
		rcoY4QIQOcPyDX1Vs4fxN61yq1RQ1qnyZ6mJkCzVi2zcrlLBOorAAUqJ0sic/I+Z
		kaPuRUaX7x63McrX2N09kr+hcwsIxh9ZQf3nZp5CHJy4E6iP0hab5UtvcFJAXFzr
		yBT5oi/hWCm+lfiZ2I7hyAQvVltr2uMMSUo6NbEBZbq955BO+VeUnNncltmkphDC
		pePuRTkCgYEAx7O60vTqXuJpN79bYOYa+M1va1NA/dqdB427wIiP99ZtGeHrpEvy
		GO2PplbgN31a/E924myWQ3Z7GvFtfGjYYqzcXHZq5c328o2oScLYc/tYdjppwMQL
		jQ6B4s1uyf05PLuCuGCPfGX8cAlfPO+xBPd2RtqGEv9zF5dMEzWBNK0CgYEAxZiC
		Ka+ypSjNQ4l5UrI58GaunmXIFcZIGKku/aqlOoVzDZWLXI582aycKc3K/VIgxN8P
		PLurDLu+s8M55RJiZnfh59ggDqonrOyLeenE3L9EI+t6VNs3nRfSodiqT202Hr2p
		F571YKhj6lFLFHGWZQHIvm0DzK14F/hUgFUNIDMCgYEAnaLN0j/p0UQ/cfXnF7IL
		kGH5lWp+XuP2GERU9EHYAvaL4GZpL6OTUwIS5malTqfw7kF7wneclVwtCLOSjSXl
		yN5Sg9olv4i5afVP5gmb+tFonsq1N6iIxaux82neDiuIxtvs78Wo/bUzcuyy9NLv
		lNAR2RQdyVlDbFfNgUw21XECgYEAnpfmuQCtGQSjo3ZeqzIjcMFpm/bDXj60NR7t
		eWoSjeL4UknZ/iLbMHbrLF5hc2sMpBcIis1x35l82ZlzCVn1IptL9SKxsDN//roo
		xGQNvsPBNDdXC26bt3mcdIyLPY7BZnEBm9TYy4i8ESDIaxM0C8Qf1D95UjlU76BA
		anRZQaMCgYASTlP6c0axoWe+YLP2zEk6CqYRfGuYeTBjZdVzOAeHo6j+18PzJ9/Z
		87JtpIvSfvR+u1s07Bshcbq2ppEcDRcWUDw6yCVSt60pHB+YLu/kTTeuyC3/r8BJ
		A2jURl0Eel11p/WRSwxCFENaAPx+mBKvZxcXORc5ocu63S6wuhZ8KA==
		-----END RSA PRIVATE KEY-----`
)
