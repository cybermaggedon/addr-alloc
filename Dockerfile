
FROM fedora:26

RUN dnf install -y libgo

COPY addr_alloc /usr/local/bin/

CMD /usr/local/bin/addr_alloc


