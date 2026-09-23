# AimiliVPN Windows

Windows &#x539F;&#x751F; VPN &#x8282;&#x70B9;&#x7BA1;&#x7406;&#x5668;&#xFF0C;&#x57FA;&#x4E8E; OpenVPN 2.6 &#x548C; Wintun&#xFF0C;&#x63D0;&#x4F9B;&#x8282;&#x70B9;&#x66F4;&#x65B0;&#x3001;&#x5BBD;&#x5E26;&#x5019;&#x9009;&#x3001;&#x7AEF;&#x53E3;&#x68C0;&#x67E5;&#x3001;VPN &#x51FA;&#x53E3;&#x9A8C;&#x8BC1;&#x3001;&#x81EA;&#x52A8;&#x6545;&#x969C;&#x5207;&#x6362;&#x548C; FlClash &#x8BA2;&#x9605;&#x3002;

## &#x529F;&#x80FD;

- &#x5408;&#x5E76; VPNGate &#x548C;&#x955C;&#x50CF;&#x6E90;&#xFF0C;&#x6BCF;&#x5C0F;&#x65F6;&#x81EA;&#x52A8;&#x5237;&#x65B0;
- &#x56FD;&#x5BB6;&#x3001;&#x8FD0;&#x8425;&#x5546;&#x3001;&#x5EF6;&#x8FDF;&#x3001;&#x6536;&#x85CF;&#x548C;&#x5019;&#x9009;&#x7C7B;&#x578B;&#x7B5B;&#x9009;
- &#x6279;&#x91CF; TCP &#x53EF;&#x8FBE;&#x6027;&#x68C0;&#x67E5;&#x548C;&#x9010;&#x8282;&#x70B9; VPN &#x51FA;&#x53E3;&#x9A8C;&#x8BC1;
- &#x81EA;&#x52A8;&#x5207;&#x6362;&#x5931;&#x8D25;&#x8282;&#x70B9;&#xFF0C;&#x4F18;&#x5148;&#x4F7F;&#x7528;&#x5DF2;&#x9A8C;&#x8BC1;&#x8282;&#x70B9;
- &#x672C;&#x673A; HTTP / SOCKS5 &#x4EE3;&#x7406;&#xFF1A;`127.0.0.1:7928`
- FlClash &#x8BA2;&#x9605;&#xFF1A;`http://127.0.0.1:7929/clash`
- &#x7BA1;&#x7406;&#x9875;&#x9762;&#xFF1A;`http://127.0.0.1:8686/`
- &#x670D;&#x52A1;&#x65E5;&#x5FD7;&#x3001;OpenVPN &#x65E5;&#x5FD7;&#x548C;&#x8FD0;&#x884C;&#x8BCA;&#x65AD;

## &#x4F7F;&#x7528;&#x65B9;&#x6CD5;

1. &#x53F3;&#x952E;&#x4EE5;&#x7BA1;&#x7406;&#x5458;&#x8EAB;&#x4EFD;&#x8FD0;&#x884C; `Start.cmd`&#x3002;
2. &#x6253;&#x5F00; `http://127.0.0.1:8686/`&#xFF0C;&#x4F7F;&#x7528; `config/Login.txt` &#x4E2D;&#x7684;&#x8D26;&#x53F7;&#x5BC6;&#x7801;&#x767B;&#x5F55;&#x3002;
3. &#x70B9;&#x51FB;&#x201C;&#x66F4;&#x65B0;&#x8282;&#x70B9;&#x201D;&#x83B7;&#x53D6;&#x5217;&#x8868;&#x3002;&#x201C;&#x68C0;&#x67E5;&#x201D;&#x53EA;&#x4EE3;&#x8868; TCP &#x7AEF;&#x53E3;&#x53EF;&#x8FBE;&#xFF0C;&#x4E0D;&#x4EE3;&#x8868; VPN &#x51FA;&#x53E3;&#x53EF;&#x7528;&#x3002;
4. &#x53EA;&#x4F7F;&#x7528;&#x9875;&#x9762;&#x663E;&#x793A;&#x201C;&#x51FA;&#x53E3;&#x5DF2;&#x9A8C;&#x8BC1;&#x8FDE;&#x901A;&#x201D;&#x5E76;&#x663E;&#x793A;&#x51FA;&#x53E3; IP &#x7684;&#x8282;&#x70B9;&#x3002;
5. &#x5C06; `http://127.0.0.1:7929/clash` &#x5BFC;&#x5165; FlClash&#x3002;
6. &#x8FD0;&#x884C; `Stop.cmd` &#x505C;&#x6B62;&#x670D;&#x52A1;&#x548C; VPN &#x5B50;&#x8FDB;&#x7A0B;&#x3002;

## &#x76EE;&#x5F55;

- `config`&#xFF1A;&#x8BBE;&#x7F6E;&#x3001;&#x6536;&#x85CF;&#x548C;&#x7BA1;&#x7406;&#x8D26;&#x53F7;
- `data`&#xFF1A;&#x8282;&#x70B9;&#x6E90;&#x3001;IP &#x4FE1;&#x606F;&#x548C;&#x5B8C;&#x6574;&#x8282;&#x70B9;&#x7F13;&#x5B58;
- `runtime`&#xFF1A;OpenVPN/Wintun &#x7EC4;&#x4EF6;&#x548C;&#x4E34;&#x65F6;&#x72B6;&#x6001;
- `logs`&#xFF1A;&#x670D;&#x52A1;&#x548C; OpenVPN &#x65E5;&#x5FD7;

&#x6BCF;&#x6B21;&#x66F4;&#x65B0;&#x540E;&#xFF0C;&#x5B8C;&#x6574;&#x8282;&#x70B9;&#x4F1A;&#x4FDD;&#x5B58;&#x5230; `data/nodes-cache.json`&#x3002;&#x4E0B;&#x6B21;&#x542F;&#x52A8;&#x4F1A;&#x4F18;&#x5148;&#x62C9;&#x53D6;&#x65B0;&#x6570;&#x636E;&#xFF0C;&#x62C9;&#x53D6;&#x5931;&#x8D25;&#x65F6;&#x4ECE;&#x7F13;&#x5B58;&#x6062;&#x590D;&#x3002;

## &#x6784;&#x5EFA;

&#x5B89;&#x88C5; Go &#x540E;&#x8FD0;&#x884C; `Build.cmd` &#x751F;&#x6210; `aimilivpn.exe`&#x3002;Windows &#x8FD0;&#x884C;&#x9700;&#x8981;&#x7BA1;&#x7406;&#x5458;&#x6743;&#x9650;&#x4EE5;&#x521B;&#x5EFA; Wintun &#x7F51;&#x5361;&#x3002;

## &#x8BF4;&#x660E;

VPNGate &#x8282;&#x70B9;&#x6765;&#x81EA;&#x516C;&#x5F00;&#x5FD7;&#x613F;&#x8005;&#x7F51;&#x7EDC;&#xFF0C;&#x53EF;&#x7528;&#x6027;&#x4F1A;&#x968F;&#x65F6;&#x95F4;&#x53D8;&#x5316;&#x3002;TCP &#x53EF;&#x8FBE;&#x4E0D;&#x4EE3;&#x8868; VPN &#x63E1;&#x624B;&#x6210;&#x529F;&#x6216;&#x51FA;&#x53E3;&#x53EF;&#x7528;&#x3002;

