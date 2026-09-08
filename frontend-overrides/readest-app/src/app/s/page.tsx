import { Suspense } from 'react';
import ShareLanding from './ShareLanding';

export default function Page() {
  return (
    <Suspense fallback={null}>
      <ShareLanding />
    </Suspense>
  );
}
