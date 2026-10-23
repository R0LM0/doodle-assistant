# Imágenes

## Importar

Se puede importar una imagen arrastrándola al canvas o desde el menú
Archivo → Importar imagen. Cada imagen importada crea automáticamente una capa nueva
del tipo imagen.

## Formatos soportados

- **PNG**: soporta transparencia. Es el formato recomendado para dibujos con fondo
  transparente.
- **JPG**: no soporta transparencia; el fondo se importa como color sólido.
- **WebP**: soporta transparencia y comprime mejor que PNG.

Los GIF animados no se importan como animación: se toma el primer frame como imagen
fija. Los SVG no están soportados.

## Límites

- Tamaño máximo de archivo: 20 MB.
- Resolución máxima: 4096 × 4096 píxeles. Una imagen más grande se rechaza con el
  error "la imagen supera el tamaño máximo permitido".
- Cantidad máxima de imágenes por proyecto: 30.

## Mover y escalar

Con la capa de imagen seleccionada, la herramienta de transformación muestra un marco
con manijas:

- arrastrar dentro del marco mueve la imagen
- arrastrar una manija de esquina escala manteniendo la proporción
- arrastrar una manija de lado escala solo en ese eje y deforma la imagen
- mantener Shift mientras escalás desde una esquina fuerza la proporción

El escalado es no destructivo: la imagen original se conserva y se vuelve a muestrear
al renderizar. Escalar una imagen mucho más allá de su resolución original la vuelve
borrosa, porque no hay píxeles nuevos que inventar.
