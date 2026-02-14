FROM busybox
ADD teanode /bin/teanode
USER nobody
ENTRYPOINT ["/bin/teanode"]
