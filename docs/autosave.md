# Autoguardado y respaldos

## Cómo funciona

El autoguardado escribe el proyecto cada 10 segundos (ver Proyectos) mientras
haya cambios sin guardar. Cada escritura exitosa genera además una copia de
respaldo.

## Copias de respaldo

Se conservan las últimas **5 copias** de respaldo por proyecto. Cuando se crea
la sexta, se borra la más vieja. Las copias viven junto al proyecto con la
extensión `.bak` numerada.
