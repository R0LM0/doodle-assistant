# Pinceles

## Pinceles disponibles

Cada pincel define cómo se convierte el trazo del lápiz en marcas sobre el canvas. El
pincel se elige desde la barra lateral izquierda y se conserva entre trazos hasta que
elijas otro.

- **Sólido**: trazo continuo de opacidad y color planos. Ideal para line art y para
  colorear por bloques. Es el pincel por defecto.
- **Crayón**: simula cera. El borde del trazo es irregular y la cobertura no es
  perfecta, así que el color se mezcla con el fondo en los bordes.
- **Marcador**: trazo ancho y translúcido. Al superponer trazos del mismo color, la
  zona compartida se oscurece, imitando un marcador real.
- **Acuarela**: acumula pigmento según la velocidad del trazo. Los bordes se abren y
  el relleno queda más denso donde pasaste más lento.
- **Neón**: trazo brillante con halo del mismo color. Se dibuja mejor sobre fondos
  oscuros; sobre fondo blanco el halo se pierde.
- **Wobbly**: al trazo recto le agrega una ondulación controlada por el parámetro de
  jiggle. Es el pincel pensado para el estilo dibujado a mano libre.

## Parámetros compartidos

Todos los pinceles respetan tres parámetros globales, ajustables con corchetes
(`[` y `]`) o desde el panel de pincel:

- grosor: de 1 a 200 píxeles
- opacidad: de 1% a 100%
- estabilizador: de 0 a 100; suaviza el temblor del pulso

## Borrado por trazo

La goma no borra píxeles: borra **trazos completos**. Si tocás cualquier punto de un
trazo con la goma, ese trazo desaparece entero de la capa activa, aunque cruce encima
de otros trazos.

Por eso el borrado es una operación reversible por deshacer: se elimina el objeto
trazo, no se pinta encima con el color del fondo. Un trazo que sigue visible en otra
capa no se ve afectado.

## Limitaciones conocidas

- El pincel Acuarela no se renderiza igual al exportar a GIF que a PNG: el GIF está
  limitado a 256 colores y los degradados suaves se ven con bandas.
- El pincel Neón no tiene halo en la exportación a WebM con fondo transparente.
